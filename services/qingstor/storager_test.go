package qingstor

import (
	"bytes"
	"errors"
	"io/ioutil"
	"testing"
	"time"

	"github.com/Xuanwo/storage/pkg/storageclass"
	"github.com/Xuanwo/storage/types/metadata"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/pengsrc/go-shared/convert"
	"github.com/stretchr/testify/assert"
	qerror "github.com/yunify/qingstor-sdk-go/v3/request/errors"
	"github.com/yunify/qingstor-sdk-go/v3/service"

	"github.com/Xuanwo/storage/pkg/segment"
	"github.com/Xuanwo/storage/types"
	"github.com/Xuanwo/storage/types/pairs"
)

func TestStorage_String(t *testing.T) {
	bucketName := "test_bucket"
	zone := "test_zone"
	c := Storage{
		workDir: "/test",
		properties: &service.Properties{
			BucketName: &bucketName,
			Zone:       &zone,
		},
	}
	assert.NotEmpty(t, c.String())
}

func TestStorage_Metadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	{
		name := uuid.New().String()
		location := uuid.New().String()

		client := Storage{
			bucket: mockBucket,
			properties: &service.Properties{
				BucketName: &name,
				Zone:       &location,
			},
		}

		m, err := client.Metadata()
		assert.NoError(t, err)
		assert.NotNil(t, m)
		assert.Equal(t, name, m.Name)
		assert.Equal(t, location, m.MustGetLocation())
	}
}

func TestStorage_Statistical(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	{
		client := Storage{
			bucket: mockBucket,
		}

		name := uuid.New().String()
		location := uuid.New().String()
		size := int64(1234)
		count := int64(4321)

		mockBucket.EXPECT().GetStatistics().DoAndReturn(func() (*service.GetBucketStatisticsOutput, error) {
			return &service.GetBucketStatisticsOutput{
				Name:     &name,
				Location: &location,
				Size:     &size,
				Count:    &count,
			}, nil
		})
		m, err := client.Statistical()
		assert.NoError(t, err)
		assert.NotNil(t, m)
	}

	{
		client := Storage{
			bucket: mockBucket,
		}

		mockBucket.EXPECT().GetStatistics().DoAndReturn(func() (*service.GetBucketStatisticsOutput, error) {
			return nil, &qerror.QingStorError{}
		})
		_, err := client.Statistical()
		assert.Error(t, err)
		assert.True(t, errors.Is(err, types.ErrUnhandledError))
	}
}

func TestStorage_AbortSegment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	client := Storage{
		bucket:   mockBucket,
		segments: make(map[string]*segment.Segment),
	}

	// Test valid segment.
	path := uuid.New().String()
	id := uuid.New().String()
	client.segments[id] = &segment.Segment{
		Path: path,
		ID:   id,
	}
	mockBucket.EXPECT().AbortMultipartUpload(gomock.Any(), gomock.Any()).Do(func(inputPath string, input *service.AbortMultipartUploadInput) {
		assert.Equal(t, path, inputPath)
		assert.Equal(t, id, *input.UploadID)
	})
	err := client.AbortSegment(id)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(client.segments))

	// Test not exist segment.
	id = uuid.New().String()
	err = client.AbortSegment(id)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, segment.ErrSegmentNotInitiated))
}

func TestStorage_CompleteSegment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		id       string
		segments map[string]*segment.Segment
		hasCall  bool
		mockFn   func(string, *service.CompleteMultipartUploadInput)
		hasError bool
		wantErr  error
	}{
		{
			"not initiated segment",
			"", map[string]*segment.Segment{},
			false, nil, true,
			segment.ErrSegmentNotInitiated,
		},
		{
			"segment part empty",
			"test_id",
			map[string]*segment.Segment{
				"test_id": {
					ID:    "test_id",
					Path:  "test_path",
					Parts: nil,
				},
			},
			false, nil,
			true, segment.ErrSegmentPartsEmpty,
		},
		{
			"valid segment",
			"test_id",
			map[string]*segment.Segment{
				"test_id": {
					ID:   "test_id",
					Path: "test_path",
					Parts: map[int64]*segment.Part{
						0: {Offset: 0, Size: 1},
					},
				},
			},
			true,
			func(inputPath string, input *service.CompleteMultipartUploadInput) {
				assert.Equal(t, "test_path", inputPath)
				assert.Equal(t, "test_id", *input.UploadID)
			},
			false, nil,
		},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			if v.hasCall {
				mockBucket.EXPECT().CompleteMultipartUpload(gomock.Any(), gomock.Any()).Do(v.mockFn)
			}

			client := Storage{
				bucket:   mockBucket,
				segments: v.segments,
			}

			err := client.CompleteSegment(v.id)
			if v.hasError {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, v.wantErr))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 0, len(client.segments))
			}
		})
	}
}

func TestStorage_Copy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		src      string
		dst      string
		mockFn   func(string, *service.PutObjectInput)
		hasError bool
		wantErr  error
	}{
		{
			"valid copy",
			"test_src", "test_dst",
			func(inputObjectKey string, input *service.PutObjectInput) {
				assert.Equal(t, "test_dst", inputObjectKey)
				assert.Equal(t, "test_src", *input.XQSCopySource)
			},
			false, nil,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().PutObject(gomock.Any(), gomock.Any()).Do(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		err := client.Copy(v.src, v.dst)
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_Delete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		src      string
		mockFn   func(string)
		hasError bool
		wantErr  error
	}{
		{
			"valid delete",
			"test_src",
			func(inputObjectKey string) {
				assert.Equal(t, "test_src", inputObjectKey)
			},
			false, nil,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().DeleteObject(gomock.Any()).Do(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		err := client.Delete(v.src)
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_InitSegment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		path     string
		segments map[string]*segment.Segment
		hasCall  bool
		mockFn   func(string, *service.InitiateMultipartUploadInput) (*service.InitiateMultipartUploadOutput, error)
		hasError bool
		wantErr  error
	}{
		{
			"valid init segment",
			"test", map[string]*segment.Segment{},
			true,
			func(inputPath string, input *service.InitiateMultipartUploadInput) (*service.InitiateMultipartUploadOutput, error) {
				assert.Equal(t, "test", inputPath)

				uploadID := "test"
				return &service.InitiateMultipartUploadOutput{
					UploadID: &uploadID,
				}, nil
			},
			false, nil,
		},
		{
			"segment already exist",
			"test",
			map[string]*segment.Segment{
				"test": {
					ID: "test_segment_id",
				},
			},
			true,
			func(inputPath string, input *service.InitiateMultipartUploadInput) (*service.InitiateMultipartUploadOutput, error) {
				assert.Equal(t, "test", inputPath)

				uploadID := "test"
				return &service.InitiateMultipartUploadOutput{
					UploadID: &uploadID,
				}, nil
			},
			false, nil,
		},
	}

	for _, v := range tests {
		if v.hasCall {
			mockBucket.EXPECT().InitiateMultipartUpload(gomock.Any(), gomock.Any()).DoAndReturn(v.mockFn)
		}

		client := Storage{
			bucket:   mockBucket,
			segments: v.segments,
		}

		_, err := client.InitSegment(v.path, pairs.WithPartSize(10))
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_List(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	keys := make([]string, 7)
	for idx := range keys {
		keys[idx] = uuid.New().String()
	}

	tests := []struct {
		name   string
		output *service.ListObjectsOutput
		items  []*types.Object
		err    error
	}{
		{
			"list without delimiter",
			&service.ListObjectsOutput{
				HasMore: service.Bool(false),
				Keys: []*service.KeyType{
					{
						Key: service.String(keys[0]),
					},
				},
			},
			[]*types.Object{
				{
					ID:         keys[0],
					Name:       keys[0],
					Type:       types.ObjectTypeFile,
					ObjectMeta: metadata.NewObjectMeta(),
				},
			},
			nil,
		},
		{
			"list with return next marker",
			&service.ListObjectsOutput{
				NextMarker: service.String("test_marker"),
				HasMore:    service.Bool(false),
				CommonPrefixes: []*string{
					service.String(keys[2]),
				},
			},
			[]*types.Object{
				{
					ID:         keys[2],
					Name:       keys[2],
					Type:       types.ObjectTypeDir,
					ObjectMeta: metadata.NewObjectMeta(),
				},
			},
			nil,
		},
		{
			"list with return empty keys",
			&service.ListObjectsOutput{
				NextMarker: service.String("test_marker"),
				HasMore:    service.Bool(true),
			},
			[]*types.Object{},
			nil,
		},
		{
			"list with error return",
			nil,
			[]*types.Object{},
			&qerror.QingStorError{
				StatusCode: 401,
			},
		},
		{
			"list with all data returned",
			&service.ListObjectsOutput{
				HasMore: service.Bool(false),
				Keys: []*service.KeyType{
					{
						Key:          service.String(keys[5]),
						MimeType:     service.String("application/json"),
						StorageClass: service.String("STANDARD"),
						Etag:         service.String("xxxxx"),
						Size:         service.Int64(1233),
						Modified:     service.Int(1233),
					},
				},
			},
			[]*types.Object{
				{
					ID:        keys[5],
					Name:      keys[5],
					Type:      types.ObjectTypeFile,
					Size:      1233,
					UpdatedAt: time.Unix(1233, 0),
					ObjectMeta: metadata.NewObjectMeta().
						SetContentType("application/json").
						SetStorageClass(storageclass.Hot).
						SetETag("xxxxx"),
				},
			},
			nil,
		},
		{
			"list with wrong storage class returned",
			&service.ListObjectsOutput{
				HasMore: service.Bool(false),
				Keys: []*service.KeyType{
					{
						Key:          service.String(keys[5]),
						MimeType:     service.String("application/json"),
						StorageClass: service.String("xxxx"),
						Etag:         service.String("xxxxx"),
						Size:         service.Int64(1233),
						Modified:     service.Int(1233),
					},
				},
			},
			[]*types.Object{},
			types.ErrStorageClassNotSupported,
		},
		{
			"list with return a dir MIME type",
			&service.ListObjectsOutput{
				HasMore: service.Bool(false),
				Keys: []*service.KeyType{
					{
						Key:      service.String(keys[6]),
						MimeType: convert.String(DirectoryContentType),
					},
				},
			},
			[]*types.Object{
				{
					ID:   keys[6],
					Name: keys[6],
					Type: types.ObjectTypeDir,
					ObjectMeta: metadata.NewObjectMeta().
						SetContentType(DirectoryContentType),
				},
			},
			nil,
		},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			path := uuid.New().String()

			mockBucket.EXPECT().ListObjects(gomock.Any()).DoAndReturn(func(input *service.ListObjectsInput) (*service.ListObjectsOutput, error) {
				assert.Equal(t, path, *input.Prefix)
				assert.Equal(t, 200, *input.Limit)
				return v.output, v.err
			})

			client := Storage{
				bucket: mockBucket,
			}

			items := make([]*types.Object, 0)

			err := client.List(path, pairs.WithDirFunc(func(object *types.Object) {
				items = append(items, object)
			}), pairs.WithFileFunc(func(object *types.Object) {
				items = append(items, object)
			}))
			assert.Equal(t, v.err == nil, err == nil)
			assert.EqualValues(t, v.items, items)
		})
	}
}

func TestStorage_Move(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		src      string
		dst      string
		mockFn   func(string, *service.PutObjectInput)
		hasError bool
		wantErr  error
	}{
		{
			"valid copy",
			"test_src", "test_dst",
			func(inputObjectKey string, input *service.PutObjectInput) {
				assert.Equal(t, "test_dst", inputObjectKey)
				assert.Equal(t, "test_src", *input.XQSMoveSource)
			},
			false, nil,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().PutObject(gomock.Any(), gomock.Any()).Do(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		err := client.Move(v.src, v.dst)
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_Read(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		path     string
		mockFn   func(string, *service.GetObjectInput) (*service.GetObjectOutput, error)
		hasError bool
		wantErr  error
	}{
		{
			"valid copy",
			"test_src",
			func(inputPath string, input *service.GetObjectInput) (*service.GetObjectOutput, error) {
				assert.Equal(t, "test_src", inputPath)
				return &service.GetObjectOutput{
					Body: ioutil.NopCloser(bytes.NewBuffer([]byte("content"))),
				}, nil
			},
			false, nil,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().GetObject(gomock.Any(), gomock.Any()).DoAndReturn(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		r, err := client.Read(v.path)
		if v.hasError {
			assert.Error(t, err)
			assert.Nil(t, r)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NotNil(t, r)
			content, rerr := ioutil.ReadAll(r)
			assert.NoError(t, rerr)
			assert.Equal(t, "content", string(content))
		}
	}
}

func TestStorage_Stat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		src      string
		mockFn   func(objectKey string, input *service.HeadObjectInput) (*service.HeadObjectOutput, error)
		hasError bool
		wantErr  error
	}{
		{
			"valid file",
			"test_src",
			func(objectKey string, input *service.HeadObjectInput) (*service.HeadObjectOutput, error) {
				assert.Equal(t, "test_src", objectKey)
				length := int64(100)
				return &service.HeadObjectOutput{
					ContentLength:   &length,
					ContentType:     convert.String("test_content_type"),
					ETag:            convert.String("test_etag"),
					XQSStorageClass: convert.String("STANDARD"),
				}, nil
			},
			false, nil,
		},
		{
			"invalid file with wrong storage class",
			"test_src",
			func(objectKey string, input *service.HeadObjectInput) (*service.HeadObjectOutput, error) {
				assert.Equal(t, "test_src", objectKey)
				length := int64(100)
				return &service.HeadObjectOutput{
					ContentLength:   &length,
					ContentType:     convert.String("test_content_type"),
					ETag:            convert.String("test_etag"),
					XQSStorageClass: convert.String("xxxx"),
				}, nil
			},
			true, types.ErrStorageClassNotSupported,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().HeadObject(gomock.Any(), gomock.Any()).DoAndReturn(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		o, err := client.Stat(v.src)
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
			assert.NotNil(t, o)
			assert.Equal(t, types.ObjectTypeFile, o.Type)
			assert.Equal(t, int64(100), o.Size)
			contentType, ok := o.GetContentType()
			assert.True(t, ok)
			assert.Equal(t, "test_content_type", contentType)
			checkSum, ok := o.GetETag()
			assert.True(t, ok)
			assert.Equal(t, "test_etag", checkSum)
			storageClass, ok := o.GetStorageClass()
			assert.True(t, ok)
			assert.Equal(t, storageclass.Hot, storageClass)
		}
	}
}

func TestStorage_Write(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		path     string
		size     int64
		mockFn   func(string, *service.PutObjectInput) (*service.PutObjectOutput, error)
		hasError bool
		wantErr  error
	}{
		{
			"valid copy",
			"test_src",
			100,
			func(inputPath string, input *service.PutObjectInput) (*service.PutObjectOutput, error) {
				assert.Equal(t, "test_src", inputPath)
				return nil, nil
			},
			false, nil,
		},
	}

	for _, v := range tests {
		mockBucket.EXPECT().PutObject(gomock.Any(), gomock.Any()).DoAndReturn(v.mockFn)

		client := Storage{
			bucket: mockBucket,
		}

		err := client.Write(v.path, nil, pairs.WithSize(v.size))
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_WriteSegment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	tests := []struct {
		name     string
		id       string
		segments map[string]*segment.Segment
		offset   int64
		size     int64
		hasCall  bool
		mockFn   func(string, *service.UploadMultipartInput) (*service.UploadMultipartOutput, error)
		hasError bool
		wantErr  error
	}{
		{
			"not initiated segment",
			"", map[string]*segment.Segment{},
			0, 1, false, nil, true,
			segment.ErrSegmentNotInitiated,
		},
		{
			"valid segment",
			"test_id",
			map[string]*segment.Segment{
				"test_id": segment.NewSegment("test_path", "test_id", 1),
			}, 0, 1,
			true,
			func(objectKey string, input *service.UploadMultipartInput) (*service.UploadMultipartOutput, error) {
				assert.Equal(t, "test_path", objectKey)
				assert.Equal(t, "test_id", *input.UploadID)

				return nil, nil
			},
			false, nil,
		},
	}

	for _, v := range tests {
		if v.hasCall {
			mockBucket.EXPECT().UploadMultipart(gomock.Any(), gomock.Any()).Do(v.mockFn)
		}

		client := Storage{
			bucket:   mockBucket,
			segments: v.segments,
		}

		err := client.WriteSegment(v.id, v.offset, v.size, nil)
		if v.hasError {
			assert.Error(t, err)
			assert.True(t, errors.Is(err, v.wantErr))
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestStorage_ListSegments(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBucket := NewMockBucket(ctrl)

	keys := make([]string, 100)
	for idx := range keys {
		keys[idx] = uuid.New().String()
	}

	tests := []struct {
		name   string
		output *service.ListMultipartUploadsOutput
		items  []*segment.Segment
		err    error
	}{
		{
			"list without delimiter",
			&service.ListMultipartUploadsOutput{
				HasMore: service.Bool(false),
				Uploads: []*service.UploadsType{
					{
						Key:      service.String(keys[0]),
						UploadID: service.String(keys[1]),
					},
				},
			},
			[]*segment.Segment{
				segment.NewSegment(keys[0], keys[1], 0),
			},
			nil,
		},
		{
			"list with return next marker",
			&service.ListMultipartUploadsOutput{
				NextKeyMarker: service.String("test_marker"),
				HasMore:       service.Bool(false),
				Uploads: []*service.UploadsType{
					{
						Key:      service.String(keys[1]),
						UploadID: service.String(keys[2]),
					},
				},
			},
			[]*segment.Segment{
				segment.NewSegment(keys[1], keys[2], 0),
			},
			nil,
		},
		{
			"list with error return",
			nil,
			[]*segment.Segment{},
			&qerror.QingStorError{
				StatusCode: 401,
			},
		},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			path := uuid.New().String()

			mockBucket.EXPECT().ListMultipartUploads(gomock.Any()).DoAndReturn(func(input *service.ListMultipartUploadsInput) (*service.ListMultipartUploadsOutput, error) {
				assert.Equal(t, path, *input.Prefix)
				assert.Equal(t, 200, *input.Limit)
				return v.output, v.err
			})

			client := Storage{
				bucket:   mockBucket,
				segments: make(map[string]*segment.Segment),
			}

			items := make([]*segment.Segment, 0)

			err := client.ListSegments(path,
				pairs.WithSegmentFunc(func(segment *segment.Segment) {
					items = append(items, segment)
				}),
			)
			assert.Equal(t, v.err == nil, err == nil)
			assert.Equal(t, v.items, items)
		})
	}
}
